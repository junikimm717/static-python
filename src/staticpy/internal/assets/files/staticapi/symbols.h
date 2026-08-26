#pragma once

#include <stddef.h>

typedef struct {
    const char *name;
    void       *address;
} ExportedSymbol;

/* Generated from Misc/stable_abi.toml by staticpy's gen package. Both tables
   are sorted in strcmp order and NULL-terminated; the counts exclude the
   terminator, and are computed in symbols.c because #ifdef'd entries mean only
   the compiler knows the real length. */
extern ExportedSymbol static_functions[];
extern ExportedSymbol static_data[];
extern const size_t static_functions_count;
extern const size_t static_data_count;
