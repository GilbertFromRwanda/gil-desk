//! Backpressure policy (planner task R-23). When frames are produced
//! faster than they can be sent, a bounded queue with a drop-oldest
//! policy is the right choice for real-time video: a frame still queued
//! by the time a newer one arrives is stale and not worth sending —
//! latency matters more than completeness. `dropped_count()` feeds the
//! `frames_dropped` metric (planner Section 14).

use std::collections::VecDeque;

pub struct FrameQueue {
    capacity: usize,
    queue: VecDeque<Vec<u8>>,
    dropped: u64,
}

impl FrameQueue {
    pub fn new(capacity: usize) -> Self {
        assert!(capacity > 0, "queue capacity must be non-zero");
        Self {
            capacity,
            queue: VecDeque::new(),
            dropped: 0,
        }
    }

    /// Pushes a frame, dropping the oldest queued frame first if the
    /// queue is already at capacity.
    pub fn push(&mut self, frame: Vec<u8>) {
        if self.queue.len() >= self.capacity {
            self.queue.pop_front();
            self.dropped += 1;
        }
        self.queue.push_back(frame);
    }

    pub fn pop(&mut self) -> Option<Vec<u8>> {
        self.queue.pop_front()
    }

    pub fn len(&self) -> usize {
        self.queue.len()
    }

    pub fn is_empty(&self) -> bool {
        self.queue.is_empty()
    }

    pub fn dropped_count(&self) -> u64 {
        self.dropped
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn retains_all_frames_under_capacity() {
        let mut q = FrameQueue::new(3);
        q.push(b"a".to_vec());
        q.push(b"b".to_vec());
        assert_eq!(q.len(), 2);
        assert_eq!(q.dropped_count(), 0);
    }

    #[test]
    fn drops_oldest_when_over_capacity() {
        let mut q = FrameQueue::new(2);
        q.push(b"a".to_vec());
        q.push(b"b".to_vec());
        q.push(b"c".to_vec()); // drops "a"

        assert_eq!(q.dropped_count(), 1);
        assert_eq!(q.pop(), Some(b"b".to_vec()));
        assert_eq!(q.pop(), Some(b"c".to_vec()));
        assert_eq!(q.pop(), None);
    }
}
