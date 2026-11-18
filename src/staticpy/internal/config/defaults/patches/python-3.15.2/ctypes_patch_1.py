# This code is meant to be injected into ctypes/__init__.py

class StaticFunc(_CFuncPtr):
    # you need pythonapi to make sure GIL stuff works fine.
    _flags_ = _FUNCFLAG_CDECL | _FUNCFLAG_PYTHONAPI
    _restype_ = c_int

class StaticCDLL:
    def __init__(self, symbol_source):
        self._symbol_source = symbol_source
        self._FuncPtr = StaticFunc
        self._handle = 0  # static; no real handle

    def __repr__(self):
        return f"<StaticCDLL 'static_symbols', handle 0x0>"

    def __getitem__(self, name):
        addr = self._resolve(name)
        if not addr:
            raise AttributeError(f"Static symbol '{name}' not found")
        return self._FuncPtr(addr)

    def __getattr__(self, name):
        if name.startswith('__') and name.endswith('__'):
            raise AttributeError(name)
        func = self.__getitem__(name)
        setattr(self, name, func)
        return func

    def _resolve(self, name):
        addr = self._symbol_source.dlsym(name)
        return cast(addr, c_void_p).value if addr else None
