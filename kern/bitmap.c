#include <linux/spinlock.h>
#include <linux/bitmap.h>
#include <linux/slab.h>

#include "bitmap.h"

int bdr_bitmap_init(struct bdr_bitmap *bm, unsigned int max_bits)
{
	if (max_bits == 0) {
		return -EINVAL;
	}

	unsigned int bitmap_size = BITS_TO_LONGS(max_bits) * sizeof(unsigned long);

	bm->bitmap = kzalloc(bitmap_size, GFP_KERNEL);
	if (!bm->bitmap) {
		return -ENOMEM;
	}

	spin_lock_init(&bm->lock);
	
	bm->max_bits = max_bits;

	return 0;
}

void bdr_bitmap_destroy(struct bdr_bitmap *bm)
{
	kfree(bm->bitmap);
}

int bdr_bitmap_allocate(struct bdr_bitmap *bm)
{
	int bit;

	if (!bm->bitmap) {
		return -EINVAL;
	}

	spin_lock(&bm->lock);

	bit = find_first_zero_bit(bm->bitmap, bm->max_bits);
	if (bit >= bm->max_bits) {
		spin_unlock(&bm->lock);
		return -ENOSPC;
	}

	set_bit(bit, bm->bitmap);

	spin_unlock(&bm->lock);

	return bit;
}

void bdr_bitmap_free(struct bdr_bitmap *bm, unsigned int bit)
{
	if (!bm->bitmap || bit >= bm->max_bits) {
		return;
	}

	spin_lock(&bm->lock);

	clear_bit(bit, bm->bitmap);

	spin_unlock(&bm->lock);
}
