#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>

extern int64_t ordinary(int64_t, int64_t);
extern int64_t packed(int64_t, int64_t);

int main(void) {
    for (int64_t x = -5; x <= 5; x++) {
        for (int64_t y = -5; y <= 5; y++) {
            printf("%" PRId64 " %" PRId64 "\n", ordinary(x, y), packed(x, y));
        }
    }
    return 0;
}
