//! C ABI boundary between the Rust core and the C++ codec layer.
//! Follows the FFI governance rules in planner Section 21:
//! extern "C" only, #[repr(C)] shared structs, opaque handles, explicit
//! ownership, no Rust panics/C++ exceptions crossing the boundary.

#[repr(C)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NdStatus {
    Ok = 0,
    ErrInvalidArg = 1,
    ErrInternal = 2,
}

/// ABI version check (planner Section 21 "ABI version check").
#[no_mangle]
pub extern "C" fn nd_ffi_abi_version() -> u32 {
    1
}

#[cfg(feature = "real-codec-link")]
extern "C" {
    fn nd_codec_abi_version() -> std::os::raw::c_uint;
}

/// Calls into the actual C++ codec library — proves the Rust<->C++
/// boundary links and executes, not just that both sides build separately
/// (Gate G0). Only compiled when `real-codec-link` is enabled.
#[cfg(feature = "real-codec-link")]
pub fn codec_abi_version() -> u32 {
    unsafe { nd_codec_abi_version() }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn abi_version_is_stable() {
        assert_eq!(nd_ffi_abi_version(), 1);
    }

    #[cfg(feature = "real-codec-link")]
    #[test]
    fn rust_calls_into_cpp_codec() {
        assert_eq!(codec_abi_version(), nd_ffi_abi_version());
    }
}
