#include "Python.h"
#include "symbols.h"
#include <string.h>

static void *find_function(const char *name) {
  for (int i = 0; static_functions[i].name != NULL; i++) {
    if (strcmp(static_functions[i].name, name) == 0) {
      return static_functions[i].address;
    }
  }
  return NULL;
}

static PyObject *py_static_dlsym(PyObject *self, PyObject *args) {
  const char *name;
  if (!PyArg_ParseTuple(args, "s", &name))
    return NULL;

  void *ptr = find_function(name);
  if (!ptr)
    Py_RETURN_NONE;

  return PyLong_FromVoidPtr(ptr);
}

static PyMethodDef StaticApiMethods[] = {
    {"dlsym", py_static_dlsym, METH_VARARGS, "Lookup static function"},
    {NULL, NULL, 0, NULL}};

static struct PyModuleDef staticapimodule = {
    PyModuleDef_HEAD_INIT, "staticapi", NULL, -1, StaticApiMethods};

PyMODINIT_FUNC PyInit_staticapi(void) {
  return PyModule_Create(&staticapimodule);
}
