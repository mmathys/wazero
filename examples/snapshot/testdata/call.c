__attribute__((noinline))
int some_fn(int a, int b) {
    return a + b;
}

int entry(int a, int b) {
    int x = some_fn(a, b);
    x += 1;
    return x;
}
