//! Safe Rust wrappers over the C ABI encoder/decoder (planner Section 6 —
//! wraps libx264/libavcodec, no codec logic implemented here or in the C++
//! layer). Only compiled when `real-codec-link` is enabled.

use std::os::raw::{c_int, c_uchar};
use std::ptr;

#[repr(C)]
struct NdEncoder {
    _private: [u8; 0],
}
#[repr(C)]
struct NdDecoder {
    _private: [u8; 0],
}

extern "C" {
    fn nd_encoder_create(width: c_int, height: c_int, fps: c_int, bitrate_kbps: c_int) -> *mut NdEncoder;
    fn nd_encoder_encode(
        encoder: *mut NdEncoder,
        y: *const c_uchar,
        y_stride: c_int,
        u: *const c_uchar,
        u_stride: c_int,
        v: *const c_uchar,
        v_stride: c_int,
        out_data: *mut *mut c_uchar,
        out_len: *mut usize,
    ) -> c_int;
    fn nd_encoder_destroy(encoder: *mut NdEncoder);

    fn nd_decoder_create() -> *mut NdDecoder;
    fn nd_decoder_decode(
        decoder: *mut NdDecoder,
        data: *const c_uchar,
        len: usize,
        out_data: *mut *mut c_uchar,
        out_len: *mut usize,
        out_width: *mut c_int,
        out_height: *mut c_int,
    ) -> c_int;
    fn nd_decoder_destroy(decoder: *mut NdDecoder);

    fn nd_buffer_free(buf: *mut c_uchar);
}

pub struct Encoder {
    ptr: *mut NdEncoder,
}

// Safe: the C++ side never shares the handle across calls concurrently;
// each Encoder value is used from one Rust owner at a time, matching how
// x264_t itself is meant to be used.
unsafe impl Send for Encoder {}

impl Encoder {
    /// Returns `None` if libx264 rejected the parameters (e.g. non-positive
    /// dimensions).
    pub fn new(width: i32, height: i32, fps: i32, bitrate_kbps: i32) -> Option<Self> {
        let ptr = unsafe { nd_encoder_create(width, height, fps, bitrate_kbps) };
        if ptr.is_null() {
            None
        } else {
            Some(Self { ptr })
        }
    }

    /// Encodes one I420 frame (`y`/`u`/`v` tightly packed at `width`/`width/2`
    /// stride). Returns `None` if the encoder buffered the frame internally
    /// without emitting output yet — not an error, keep feeding frames.
    pub fn encode(&mut self, y: &[u8], u: &[u8], v: &[u8], width: i32) -> Option<Vec<u8>> {
        let mut out_data: *mut c_uchar = ptr::null_mut();
        let mut out_len: usize = 0;
        let status = unsafe {
            nd_encoder_encode(
                self.ptr,
                y.as_ptr(),
                width,
                u.as_ptr(),
                width / 2,
                v.as_ptr(),
                width / 2,
                &mut out_data,
                &mut out_len,
            )
        };
        assert_eq!(status, 0, "nd_encoder_encode returned error status {status}");
        if out_len == 0 {
            return None;
        }
        let bytes = unsafe { std::slice::from_raw_parts(out_data, out_len).to_vec() };
        unsafe { nd_buffer_free(out_data) };
        Some(bytes)
    }
}

impl Drop for Encoder {
    fn drop(&mut self) {
        unsafe { nd_encoder_destroy(self.ptr) };
    }
}

pub struct DecodedFrame {
    pub width: i32,
    pub height: i32,
    /// I420: Y plane, then U, then V, each tightly packed.
    pub data: Vec<u8>,
}

pub struct Decoder {
    ptr: *mut NdDecoder,
}

unsafe impl Send for Decoder {}

impl Decoder {
    pub fn new() -> Option<Self> {
        let ptr = unsafe { nd_decoder_create() };
        if ptr.is_null() {
            None
        } else {
            Some(Self { ptr })
        }
    }

    /// Decodes one Annex B H.264 access unit. Returns `None` if the decoder
    /// needs more input before it can produce a picture — not an error.
    pub fn decode(&mut self, data: &[u8]) -> Option<DecodedFrame> {
        let mut out_data: *mut c_uchar = ptr::null_mut();
        let mut out_len: usize = 0;
        let mut width: c_int = 0;
        let mut height: c_int = 0;
        let status = unsafe {
            nd_decoder_decode(
                self.ptr,
                data.as_ptr(),
                data.len(),
                &mut out_data,
                &mut out_len,
                &mut width,
                &mut height,
            )
        };
        assert_eq!(status, 0, "nd_decoder_decode returned error status {status}");
        if out_len == 0 {
            return None;
        }
        let bytes = unsafe { std::slice::from_raw_parts(out_data, out_len).to_vec() };
        unsafe { nd_buffer_free(out_data) };
        Some(DecodedFrame {
            width,
            height,
            data: bytes,
        })
    }
}

impl Drop for Decoder {
    fn drop(&mut self) {
        unsafe { nd_decoder_destroy(self.ptr) };
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn synthetic_i420_frame(width: i32, height: i32) -> (Vec<u8>, Vec<u8>, Vec<u8>) {
        let w = width as usize;
        let h = height as usize;
        let y: Vec<u8> = (0..w * h).map(|i| ((i / w + i % w) & 0xFF) as u8).collect();
        let u: Vec<u8> = (0..(w * h / 4)).map(|i| (i & 0xFF) as u8).collect();
        let v: Vec<u8> = (0..(w * h / 4)).map(|i| ((i * 3) & 0xFF) as u8).collect();
        (y, u, v)
    }

    #[test]
    fn encoder_rejects_invalid_dimensions() {
        assert!(Encoder::new(0, 0, 30, 1000).is_none());
    }

    #[test]
    fn encode_decode_round_trip_through_rust_wrapper() {
        let width = 64;
        let height = 48;
        let (y, u, v) = synthetic_i420_frame(width, height);

        let mut encoder = Encoder::new(width, height, 30, 500).expect("encoder creation failed");
        let mut decoder = Decoder::new().expect("decoder creation failed");

        let mut decoded = None;
        for _ in 0..5 {
            let Some(encoded) = encoder.encode(&y, &u, &v, width) else {
                continue;
            };
            if let Some(frame) = decoder.decode(&encoded) {
                decoded = Some(frame);
                break;
            }
        }

        let frame = decoded.expect("decoder never produced a frame within 5 input frames");
        assert_eq!(frame.width, width);
        assert_eq!(frame.height, height);
        assert_eq!(frame.data.len(), (width * height * 3 / 2) as usize);
    }
}
