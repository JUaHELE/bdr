#ifndef BDR_BITMAP_H
#define BDR_BITMAP_H

struct bdr_bitmap {
	unsigned long *bitmap;
	spinlock_t lock;
	unsigned long max_bits;
};

int bdr_bitmap_init(struct bdr_bitmap *bm, unsigned int max_bits);

void bdr_bitmap_destroy(struct bdr_bitmap *bm);

int bdr_bitmap_allocate(struct bdr_bitmap *bm);

void bdr_bitmap_free(struct bdr_bitmap *bm, unsigned int bit);

#endif
