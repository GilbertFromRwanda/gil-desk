// Compiles the C++ codec directly into this crate when `real-codec-link` is
// enabled (see Cargo.toml). This is deliberately not CMake — cc::Build calls
// the same compiler CMake would, without requiring CMake itself, which
// keeps this buildable from a plain `cargo build` inside any image/host
// that has a C++ compiler (plus libx264/libavcodec dev packages).
fn main() {
    #[cfg(feature = "real-codec-link")]
    {
        let x264 = pkg_config::probe_library("x264").expect("libx264 dev package not found");
        let avcodec =
            pkg_config::probe_library("libavcodec").expect("libavcodec dev package not found");
        let avutil =
            pkg_config::probe_library("libavutil").expect("libavutil dev package not found");

        let mut build = cc::Build::new();
        build
            .cpp(true)
            .file("../../codec/src/codec.cpp")
            .file("../../codec/src/encoder.cpp")
            .file("../../codec/src/decoder.cpp")
            .include("../../codec/include");
        for lib in [&x264, &avcodec, &avutil] {
            for path in &lib.include_paths {
                build.include(path);
            }
        }
        build.compile("nexdesk_codec");

        println!("cargo:rerun-if-changed=../../codec/src/codec.cpp");
        println!("cargo:rerun-if-changed=../../codec/src/encoder.cpp");
        println!("cargo:rerun-if-changed=../../codec/src/decoder.cpp");
        println!("cargo:rerun-if-changed=../../codec/include/nexdesk/codec.h");
    }
}
