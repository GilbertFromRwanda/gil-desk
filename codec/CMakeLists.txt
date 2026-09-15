cmake_minimum_required(VERSION 3.20)
project(nexdesk_codec LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# Encoder/decoder wrap libx264 / FFmpeg per the planner's codec decision
# (Section 2) rather than a from-scratch implementation — do not add a
# hand-rolled H.264 encoder/decoder here.
find_package(PkgConfig REQUIRED)
pkg_check_modules(X264 REQUIRED x264)
pkg_check_modules(AVCODEC REQUIRED libavcodec libavutil)

add_library(nexdesk_codec
    src/codec.cpp
    src/encoder.cpp
    src/decoder.cpp
)
target_include_directories(nexdesk_codec PUBLIC include)
target_include_directories(nexdesk_codec PRIVATE ${X264_INCLUDE_DIRS} ${AVCODEC_INCLUDE_DIRS})
target_link_libraries(nexdesk_codec PRIVATE ${X264_LIBRARIES} ${AVCODEC_LIBRARIES})
target_link_directories(nexdesk_codec PRIVATE ${X264_LIBRARY_DIRS} ${AVCODEC_LIBRARY_DIRS})

enable_testing()
add_subdirectory(tests)
