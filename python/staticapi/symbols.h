#pragma once

typedef struct {
    const char* name;
    void*       address;
} ExportedSymbol;

extern ExportedSymbol static_functions[];
