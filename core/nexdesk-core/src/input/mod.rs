//! Keyboard/mouse/scroll input event encoding (planner task R-22).

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum InputEvent {
    KeyDown(u32),
    KeyUp(u32),
    MouseMove { x: i32, y: i32 },
    MouseButton { button: u8, down: bool },
    Scroll { dx: i32, dy: i32 },
}
