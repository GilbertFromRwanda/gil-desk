// Compiles the C++ codec directly into this crate when `real-codec-link` is
// enabled (see Cargo.toml). This is deliberately not CMake — cc::Build calls
// the same compiler CMake would, without requiring CMake itself, which
// keeps this buildable from a plain `cargo build` inside any image/host
// that has a C++ compiler.
fn main() {
    #[cfg(feature = "real-codec-link")]
    {
        cc::Build::new()
            .cpp(true)
            .file("../../codec/src/codec.cpp")
            .include("../../codec/include")
            .compile("nexdesk_codec");
        println!("cargo:rerun-if-changed=../../codec/src/codec.cpp");
        println!("cargo:rerun-if-changed=../../codec/include/nexdesk/codec.h");
    }
}
