#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

int main(int argc, char **argv) {
  ssize_t n, m;
  char buf[BUFSIZ];

  if (argc != 2) {
    fprintf(stderr, "usage: %s <from>\n", argv[0]);
    exit(1);
  }

  int in = open(argv[1], O_RDONLY);
  if (in < 0) {
    fprintf(stderr, "error opening input %s: %s\n", argv[1], strerror(errno));
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