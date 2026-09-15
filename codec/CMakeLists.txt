cmake_minimum_required(VERSION 3.20)
project(nexdesk_codec LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

add_library(nexdesk_codec
    src/codec.cpp
)
target_include_directories(nexdesk_codec PUBLIC include)

# Encoder/decoder wrap libx264 / FFmpeg per the planner's codec decision
# (Section 2) rather than a from-scratch implementation — do not add a
# hand-rolled H.264 encoder here.

enable_testing()
add_subdirectory(tests)
