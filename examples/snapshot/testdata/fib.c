__attribute__((noinline)) int fib(int a, int b) {
  if (a <= 1) {
    return a;
  }

  return fib(a - 1, b) + fib(a - 2, b);
}

int entry(int a, int b) {
  int x = fib(a, b);
  return x;
}
