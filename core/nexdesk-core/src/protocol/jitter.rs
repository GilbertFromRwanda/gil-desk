//! Jitter buffer (planner task R-21). Frames can arrive out of order or
//! with variable delay; this holds them briefly to smooth that out, but
//! never blocks forever on one missing frame — real-time media favors
//! moving forward over waiting for a late frame that's no longer useful
//! by the time it'd arrive. Takes an explicit `now`/`arrived_at` instead
//! of reading the wall clock, so it's testable deterministically without
//! sleeping in tests.

use std::collections::BTreeMap;
use std::time::{Duration, Instant};

pub struct JitterBuffer {
    max_wait: Duration,
    next_seq: u64,
    buffered: BTreeMap<u64, (Instant, Vec<u8>)>,
}

impl JitterBuffer {
    pub fn new(max_wait: Duration) -> Self {
        Self {
            max_wait,
            next_seq: 0,
            buffered: BTreeMap::new(),
        }
    }

    /// Buffers a frame. Frames at or before the last-delivered sequence
    /// number are stale duplicates and are silently dropped.
    pub fn push(&mut self, seq: u64, data: Vec<u8>, arrived_at: Instant) {
        if seq < self.next_seq {
            return;
        }
        self.buffered.insert(seq, (arrived_at, data));
    }

    /// Returns the next frame ready for playout as of `now`: the
    /// next-in-sequence frame if it has arrived, or — once we've been
    /// waiting past `max_wait` for it — the earliest frame we do have,
    /// skipping the gap left by whatever didn't arrive.
    pub fn pop_ready(&mut self, now: Instant) -> Option<Vec<u8>> {
        if let Some((_, data)) = self.buffered.remove(&self.next_seq) {
            self.next_seq += 1;
            return Some(data);
        }

        let earliest = self
            .buffered
            .iter()
            .next()
            .map(|(&seq, (arrived_at, _))| (seq, *arrived_at));

        if let Some((seq, arrived_at)) = earliest {
            if now.duration_since(arrived_at) >= self.max_wait {
                let (_, data) = self.buffered.remove(&seq).expect("just looked up");
                self.next_seq = seq + 1;
                return Some(data);
            }
        }

        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn releases_in_sequence_order_once_available() {
        let mut jb = JitterBuffer::new(Duration::from_millis(50));
        let t0 = Instant::now();
        jb.push(0, b"a".to_vec(), t0);
        jb.push(1, b"b".to_vec(), t0);

        assert_eq!(jb.pop_ready(t0), Some(b"a".to_vec()));
        assert_eq!(jb.pop_ready(t0), Some(b"b".to_vec()));
        assert_eq!(jb.pop_ready(t0), None);
    }

    #[test]
    fn reorders_frames_that_arrive_out_of_order() {
        let mut jb = JitterBuffer::new(Duration::from_millis(50));
        let t0 = Instant::now();
        jb.push(2, b"c".to_vec(), t0);
        jb.push(0, b"a".to_vec(), t0);
        jb.push(1, b"b".to_vec(), t0);

        assert_eq!(jb.pop_ready(t0), Some(b"a".to_vec()));
        assert_eq!(jb.pop_ready(t0), Some(b"b".to_vec()));
        assert_eq!(jb.pop_ready(t0), Some(b"c".to_vec()));
    }

    #[test]
    fn waits_for_a_gap_up_to_max_wait_then_skips_it() {
        let mut jb = JitterBuffer::new(Duration::from_millis(50));
        let t0 = Instant::now();
        // seq 1 never arrives.
        jb.push(2, b"c".to_vec(), t0);

        assert_eq!(jb.pop_ready(t0), None, "still within max_wait");
        let past_deadline = t0 + Duration::from_millis(51);
        assert_eq!(jb.pop_ready(past_deadline), Some(b"c".to_vec()));

        // The gap is now behind next_seq; a late arrival for it is stale.
        jb.push(1, b"late".to_vec(), past_deadline);
        assert_eq!(jb.pop_ready(past_deadline), None);
    }

    #[test]
    fn drops_stale_duplicates() {
        let mut jb = JitterBuffer::new(Duration::from_millis(50));
        let t0 = Instant::now();
        jb.push(0, b"a".to_vec(), t0);
        assert_eq!(jb.pop_ready(t0), Some(b"a".to_vec()));

        jb.push(0, b"replayed".to_vec(), t0); // seq 0 already delivered
        assert_eq!(jb.pop_ready(t0), None);
    }
}
