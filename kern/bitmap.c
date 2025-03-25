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
	if (!bm->bitmap) {
		return;
	}

	kfree(bm->bitmap);
}

unsigned int bdr_bitmap_get_byte_size(struct bdr_bitmap *bm)
{
	if (!bm || !bm->bitmap) {
		return 0;
	}

	return (bm->max_bits + 7) / 8;
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

void bdr_bitmap_set(struct bdr_bitmap *bm, unsigned int bit)
{
	if (!bm->bitmap || bit >= bm->max_bits) {
		return;
	}

	spin_lock(&bm->lock);
	set_bit(bit, bm->bitmap);
	spin_unlock(&bm->lock);
}

void bdr_bitmap_clear(struct bdr_bitmap *bm)
{
	if (!bm->bitmap) {
		return;
	}

	spin_lock(&bm->lock);
	memset(bm->bitmap, 0, BITS_TO_LONGS(bm->max_bits) * sizeof(unsigned long));
	spin_unlock(&bm->lock);
}

void bdr_bitmap_set_sector(struct bdr_bitmap *bm, unsigned long sector)
{
	unsigned long block = sector >> BDR_BLOCK_SIZE_SHIFT;

	pr_info("Sector %lu marked", block);
	
	spin_lock(&bm->lock);
	set_bit(block, bm->bitmap);
	spin_unlock(&bm->lock);
}


void bdr_bitmap_print(struct bdr_bitmap *bm)
{
	if (!bm || !bm->bitmap) {
		pr_err("Invalid bitmap or uninitialized bitmap\n");
		return;
	}

	// Calculate number of longs (words) in the bitmap
	unsigned int bitmap_words = BITS_TO_LONGS(bm->max_bits);

	pr_info("Bitmap Information:");
	pr_info("  Max Bits: %lu", bm->max_bits);
	pr_info("  Bitmap Words: %u", bitmap_words);

	pr_info("Bitmap Contents (Set Bits):");

	// Iterate through each bit and print set bits
	for (unsigned int bit = 0; bit < bm->max_bits; bit++) {
		if (test_bit(bit, bm->bitmap)) {
			pr_info("  Bit %u is set", bit);
		}
	}

	// Optional: Print raw bitmap contents for debugging
	pr_info("Raw Bitmap Hex Representation:");
	for (unsigned int i = 0; i < bitmap_words; i++) {
		pr_info("  Word %u: 0x%lx", i, bm->bitmap[i]);
	}
}
