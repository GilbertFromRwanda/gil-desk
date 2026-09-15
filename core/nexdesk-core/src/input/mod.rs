//! Keyboard/mouse/scroll input event encoding (planner task R-22). A
//! hand-rolled compact binary format rather than protobuf — input events
//! are small, extremely high-frequency messages where the framing/tag
//! overhead of a general-purpose format is real cost, and the format
//! itself is trivial enough not to need a schema. Decoding untrusted
//! network bytes must never panic, so every read is bounds-checked.

use crate::error::{NexError, Result};

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum InputEvent {
    KeyDown(u32),
    KeyUp(u32),
    MouseMove { x: i32, y: i32 },
    MouseButton { button: u8, down: bool },
    Scroll { dx: i32, dy: i32 },
}

impl InputEvent {
    pub fn encode(&self) -> Vec<u8> {
        let mut buf = Vec::new();
        match *self {
            InputEvent::KeyDown(code) => {
                buf.push(0);
                buf.extend_from_slice(&code.to_be_bytes());
            }
            InputEvent::KeyUp(code) => {
                buf.push(1);
                buf.extend_from_slice(&code.to_be_bytes());
            }
            InputEvent::MouseMove { x, y } => {
                buf.push(2);
                buf.extend_from_slice(&x.to_be_bytes());
                buf.extend_from_slice(&y.to_be_bytes());
            }
            InputEvent::MouseButton { button, down } => {
                buf.push(3);
                buf.push(button);
                buf.push(down as u8);
            }
            InputEvent::Scroll { dx, dy } => {
                buf.push(4);
                buf.extend_from_slice(&dx.to_be_bytes());
                buf.extend_from_slice(&dy.to_be_bytes());
            }
        }
        buf
    }

    pub fn decode(bytes: &[u8]) -> Result<Self> {
        let tag = *bytes
            .first()
            .ok_or_else(|| NexError::Transport("empty input event".to_string()))?;
        let rest = &bytes[1..];

        match tag {
            0 => Ok(InputEvent::KeyDown(take_u32(rest, 0)?)),
            1 => Ok(InputEvent::KeyUp(take_u32(rest, 0)?)),
            2 => Ok(InputEvent::MouseMove {
                x: take_i32(rest, 0)?,
                y: take_i32(rest, 4)?,
            }),
            3 => {
                let button = *rest
                    .first()
                    .ok_or_else(|| NexError::Transport("truncated input event".to_string()))?;
                let down = *rest
                    .get(1)
                    .ok_or_else(|| NexError::Transport("truncated input event".to_string()))?
                    != 0;
                Ok(InputEvent::MouseButton { button, down })
            }
            4 => Ok(InputEvent::Scroll {
                dx: take_i32(rest, 0)?,
                dy: take_i32(rest, 4)?,
            }),
            other => Err(NexError::Transport(format!(
                "unknown input event tag {other}"
            ))),
        }
    }
}

fn take_u32(bytes: &[u8], offset: usize) -> Result<u32> {
    let arr: [u8; 4] = bytes
        .get(offset..offset + 4)
        .and_then(|s| s.try_into().ok())
        .ok_or_else(|| NexError::Transport("truncated input event".to_string()))?;
    Ok(u32::from_be_bytes(arr))
}

fn take_i32(bytes: &[u8], offset: usize) -> Result<i32> {
    Ok(take_u32(bytes, offset)? as i32)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn round_trips(event: InputEvent) {
        let bytes = event.encode();
        assert_eq!(InputEvent::decode(&bytes).unwrap(), event);
    }

    #[test]
    fn round_trips_every_variant() {
        round_trips(InputEvent::KeyDown(65));
        round_trips(InputEvent::KeyUp(65));
        round_trips(InputEvent::MouseMove { x: -12, y: 900 });
        round_trips(InputEvent::MouseButton {
            button: 1,
            down: true,
        });
        round_trips(InputEvent::Scroll { dx: 0, dy: -3 });
    }

    #[test]
    fn decode_never_panics_on_malformed_input() {
        // Every tag with a truncated (or absent) payload must error, not panic.
        for tag in 0u8..=5 {
            for len in 0..8 {
                let mut bytes = vec![tag];
                bytes.extend(std::iter::repeat_n(0u8, len));
                let _ = InputEvent::decode(&bytes); // must not panic
            }
        }
        assert!(InputEvent::decode(&[]).is_err());
    }

    #[test]
    fn decode_rejects_unknown_tag() {
        assert!(InputEvent::decode(&[99, 0, 0, 0, 0]).is_err());
    }
}
