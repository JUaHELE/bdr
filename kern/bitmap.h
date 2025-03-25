#ifndef BDR_BITMAP_H
#define BDR_BITMAP_H

#define BDR_BLOCK_SIZE_SHIFT 11  // 2^11 sectors = 1MB (512 * 2048 = 1MB)
#define BDR_BLOCK_SIZE (1 << BDR_BLOCK_SIZE_SHIFT)  // 2048 sectors = 1MB
#define BDR_BYTE_SIZE 8

struct bdr_bitmap {
	unsigned long *bitmap;
	spinlock_t lock;
	unsigned long max_bits;
};

int bdr_bitmap_init(struct bdr_bitmap *bm, unsigned int max_bits);

void bdr_bitmap_destroy(struct bdr_bitmap *bm);

int bdr_bitmap_allocate(struct bdr_bitmap *bm);

void bdr_bitmap_free(struct bdr_bitmap *bm, unsigned int bit);

void bdr_bitmap_set(struct bdr_bitmap *bm, unsigned int bit);

void bdr_bitmap_clear(struct bdr_bitmap *bm);

unsigned int bdr_bitmap_get_byte_size(struct bdr_bitmap *bm);

void bdr_bitmap_set_sector(struct bdr_bitmap *bm, unsigned long sector);

void bdr_bitmap_print(struct bdr_bitmap *bm);
#endif
