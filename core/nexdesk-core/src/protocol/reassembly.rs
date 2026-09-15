//! Frame reassembly (planner task R-20). Chunks belonging to a frame may
//! arrive out of order, and multiple frames' chunks may be interleaved;
//! this buffers them per `frame_id` until every chunk has arrived, then
//! hands back one contiguous payload. Bounded: only `max_frames_buffered`
//! incomplete frames are held at once, oldest evicted first — an
//! unbounded buffer here is exactly what the planner's memory-leak/soak
//! testing concerns exist to catch.

use crate::error::{NexError, Result};
use std::collections::{HashMap, VecDeque};

#[derive(Debug, Clone)]
pub struct Chunk {
    pub frame_id: u64,
    pub chunk_index: u32,
    pub total_chunks: u32,
    pub data: Vec<u8>,
}

struct PartialFrame {
    total_chunks: u32,
    received: HashMap<u32, Vec<u8>>,
}

pub struct FrameReassembler {
    max_frames_buffered: usize,
    partial: HashMap<u64, PartialFrame>,
    order: VecDeque<u64>,
}

impl FrameReassembler {
    pub fn new(max_frames_buffered: usize) -> Self {
        assert!(max_frames_buffered > 0, "must buffer at least one frame");
        Self {
            max_frames_buffered,
            partial: HashMap::new(),
            order: VecDeque::new(),
        }
    }

    /// Feeds one chunk. Returns the reassembled frame once every chunk for
    /// its `frame_id` has arrived, or `None` while still incomplete.
    pub fn accept(&mut self, chunk: Chunk) -> Result<Option<Vec<u8>>> {
        if chunk.total_chunks == 0 || chunk.chunk_index >= chunk.total_chunks {
            return Err(NexError::Transport(format!(
                "invalid chunk: index {} of {} total",
                chunk.chunk_index, chunk.total_chunks
            )));
        }

        if !self.partial.contains_key(&chunk.frame_id) {
            if self.partial.len() >= self.max_frames_buffered {
                if let Some(oldest) = self.order.pop_front() {
                    self.partial.remove(&oldest);
                }
            }
            self.partial.insert(
                chunk.frame_id,
                PartialFrame {
                    total_chunks: chunk.total_chunks,
                    received: HashMap::new(),
                },
            );
            self.order.push_back(chunk.frame_id);
        }

        let frame_id = chunk.frame_id;
        let is_complete = {
            let partial = self.partial.get_mut(&frame_id).expect("just inserted above");
            partial.received.insert(chunk.chunk_index, chunk.data);
            partial.received.len() as u32 == partial.total_chunks
        };

        if !is_complete {
            return Ok(None);
        }

        let partial = self.partial.remove(&frame_id).expect("checked complete above");
        self.order.retain(|&id| id != frame_id);

        let mut bytes = Vec::new();
        for i in 0..partial.total_chunks {
            bytes.extend_from_slice(&partial.received[&i]);
        }
        Ok(Some(bytes))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn chunk(frame_id: u64, chunk_index: u32, total_chunks: u32, data: &[u8]) -> Chunk {
        Chunk {
            frame_id,
            chunk_index,
            total_chunks,
            data: data.to_vec(),
        }
    }

    #[test]
    fn reassembles_in_order_chunks() {
        let mut r = FrameReassembler::new(4);
        assert!(r.accept(chunk(1, 0, 2, b"hel")).unwrap().is_none());
        let result = r.accept(chunk(1, 1, 2, b"lo")).unwrap();
        assert_eq!(result, Some(b"hello".to_vec()));
    }

    #[test]
    fn reassembles_out_of_order_chunks() {
        let mut r = FrameReassembler::new(4);
        assert!(r.accept(chunk(1, 2, 3, b"C")).unwrap().is_none());
        assert!(r.accept(chunk(1, 0, 3, b"A")).unwrap().is_none());
        let result = r.accept(chunk(1, 1, 3, b"B")).unwrap();
        assert_eq!(result, Some(b"ABC".to_vec()));
    }

    #[test]
    fn interleaves_independent_frames() {
        let mut r = FrameReassembler::new(4);
        assert!(r.accept(chunk(1, 0, 2, b"X")).unwrap().is_none());
        assert!(r.accept(chunk(2, 0, 1, b"Y")).unwrap().is_some());
        let result = r.accept(chunk(1, 1, 2, b"Z")).unwrap();
        assert_eq!(result, Some(b"XZ".to_vec()));
    }

    #[test]
    fn evicts_oldest_incomplete_frame_when_buffer_is_full() {
        let mut r = FrameReassembler::new(1);
        assert!(r.accept(chunk(1, 0, 2, b"A")).unwrap().is_none()); // frame 1 buffered
        assert!(r.accept(chunk(2, 0, 2, b"B")).unwrap().is_none()); // evicts frame 1

        // Frame 1 is gone; completing it now starts a brand-new (still
        // incomplete) entry rather than resuming the evicted one.
        assert!(r.accept(chunk(1, 1, 2, b"A2")).unwrap().is_none());
    }

    #[test]
    fn rejects_a_chunk_index_out_of_range() {
        let mut r = FrameReassembler::new(4);
        assert!(r.accept(chunk(1, 3, 2, b"bad")).unwrap_err().to_string().contains("invalid chunk"));
    }
}
