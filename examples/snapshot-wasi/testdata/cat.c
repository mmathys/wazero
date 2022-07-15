#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) { return 0; }

__attribute__((export_name("entry")))
int entry() {
  ssize_t n, m;
  char buf[BUFSIZ];

  int in = open("test.txt", O_RDONLY);
  if (in < 0) {
    fprintf(stderr, "error opening input %s: %s\n", "test.txt", strerror(errno));
    exit(1);
  }

  while ((n = read(in, buf, BUFSIZ)) > 0) {
    char *ptr = buf;
    while (n > 0) {
      m = write(STDOUT_FILENO, ptr, (size_t)n);
      if (m < 0) {
        fprintf(stderr, "write error: %s\n", strerror(errno));
        exit(1);
      }
      n -= m;
      ptr += m;
    }
  }

  if (n < 0) {
    fprintf(stderr, "read error: %s\n", strerror(errno));
    exit(1);
  }

  return EXIT_SUCCESS;
}