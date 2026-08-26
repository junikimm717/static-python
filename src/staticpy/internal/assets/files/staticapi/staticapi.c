#include "Python.h"
#include "symbols.h"
#include <stdlib.h>
#include <string.h>

static int symbol_cmp(const void *key, const void *elem) {
  return strcmp((const char *)key, ((const ExportedSymbol *)elem)->name);
}

static void *find_in(ExportedSymbol *table, size_t count, const char *name) {
  ExportedSymbol *hit =
      bsearch(name, table, count, sizeof(ExportedSymbol), symbol_cmp);
  return hit ? hit->address : NULL;
}

/* A data symbol's address must never be called, so the kind travels with it. */
static void *find_symbol(const char *name, const char **kind) {
  void *ptr = find_in(static_functions, static_functions_count, name);
  if (ptr) {
    *kind = "func";
    return ptr;
  }
  ptr = find_in(static_data, static_data_count, name);
  if (ptr) {
    *kind = "data";
  }
  return ptr;
}

static PyObject *py_static_dlsym(PyObject *self, PyObject *args) {
  const char *name;
  if (!PyArg_ParseTuple(args, "s", &name))
    return NULL;

  const char *kind = NULL;
  void *ptr = find_symbol(name, &kind);
  if (!ptr)
    Py_RETURN_NONE;

  return PyLong_FromVoidPtr(ptr);
}

static PyObject *py_static_dlsym_ex(PyObject *self, PyObject *args) {
  const char *name;
  if (!PyArg_ParseTuple(args, "s", &name))
    return NULL;

  const char *kind = NULL;
  void *ptr = find_symbol(name, &kind);
  if (!ptr)
    Py_RETURN_NONE;

  return Py_BuildValue("(Ns)", PyLong_FromVoidPtr(ptr), kind);
}

static PyMethodDef StaticApiMethods[] = {
    {"dlsym", py_static_dlsym, METH_VARARGS, "Address of a stable-ABI symbol"},
    {"dlsym_ex", py_static_dlsym_ex, METH_VARARGS,
     "(address, 'func'|'data') for a stable-ABI symbol"},
    {NULL, NULL, 0, NULL}};

static struct PyModuleDef staticapimodule = {
    PyModuleDef_HEAD_INIT, "staticapi", NULL, -1, StaticApiMethods};

PyMODINIT_FUNC PyInit_staticapi(void) {
  return PyModule_Create(&staticapimodule);
}
