#include <linux/kernel.h>
#include <linux/module.h>
#include <linux/blkdev.h>
#include <linux/device.h>
#include <linux/version.h>
#include <linux/types.h>
#include <linux/kdev_t.h>
#include <linux/fs.h>
#include <linux/cdev.h>
#include <linux/vmalloc.h>
#include <linux/mm.h>
#include <linux/wait.h>

#include <linux/module.h>
#include <linux/kernel.h>
#include <linux/init.h>
#include <linux/bio.h>
#include <linux/device-mapper.h>

#include "ring-buffer.h"


// TODO:__attribute__((aligned(64)))

/*
 * initiates and allocates resources
 * size is a number which describes how many actual writes fit into the buffer
 */
int bdr_ring_buffer_init(struct bdr_ring_buffer *rb, unsigned int max_writes)
{
	unsigned long buf_size;

	if (!rb || max_writes == 0) {
		return -EINVAL;
	}
	
	buf_size = BDR_WRITES_TO_BYTES(max_writes);

	rb->buffer = vmalloc(PAGE_ALIGN(buf_size));
	if(!rb->buffer) {
		return -ENOMEM;
	}

	memset(&rb->buffer_info, 0, sizeof(rb->buffer_info));
	rb->buffer_info.max_writes = max_writes;

	spin_lock_init(&rb->lock);

	return 0;
}

/*
 * frees alocated resources within ring buffer
 */
void bdr_ring_buffer_free(struct bdr_ring_buffer *rb)
{
	if (rb && rb->buffer) {
		vfree(rb->buffer);
		rb->buffer = NULL;
	}
}


bool bdr_ring_buffer_is_empty(struct bdr_ring_buffer *rb)
{
	bool empty;

	spin_lock(&rb->lock);
	empty = (rb->buffer_info.last == rb->buffer_info.offset);
	spin_unlock(&rb->lock);

	return empty;
}

/*
 * gets information from the ring buffer
 */
struct bdr_buffer_info
bdr_ring_buffer_get_info(struct bdr_ring_buffer *rb)
{
	struct bdr_buffer_info buffer_info;

	spin_lock(&rb->lock);
	buffer_info = rb->buffer_info;
	spin_unlock(&rb->lock);

	return buffer_info;
}

/*
 * checks if buffer overflown
 */
bool bdr_ring_buffer_is_full(struct bdr_ring_buffer *rb)
{
	bool full;

	spin_lock(&rb->lock);
	full = rb->buffer_info.flags & BDR_OVERFLOWN_BUFFER_FLAG;
	spin_unlock(&rb->lock);

	return full;
}

void bdr_ring_buffer_update_unsafe(struct bdr_ring_buffer *rb)
{
	struct bdr_buffer_info *buffer_info;
	unsigned long buffer_offset;

	buffer_info = &rb->buffer_info;

	buffer_info->length += 1;

	/* in this function we also check whether the buffer overflown: */
	buffer_offset = (buffer_info->offset + buffer_info->length) % buffer_info->max_writes;

	if (buffer_offset == buffer_info->last) {
		buffer_info->flags |= BDR_OVERFLOWN_BUFFER_FLAG;
	}
}

void bdr_ring_buffer_reset(struct bdr_ring_buffer *rb)
{
	spin_lock(&rb->lock);
	rb->buffer_info.offset = 0;
	rb->buffer_info.length = 0;
	rb->buffer_info.last = 0;
	rb->buffer_info.flags = 0;
	spin_unlock(&rb->lock);
}


/*
 * is called whenever userpace wants to know location of new writes
 * the previous buffer info is passed to the userspace and new offsets are
 * appropriately calculated
 */
struct bdr_buffer_info bdr_buffer_read_routine(struct bdr_ring_buffer *rb) {
	struct bdr_buffer_info buffer_info;
	bool is_full;
	
	spin_lock(&rb->lock);
	
	buffer_info = rb->buffer_info;

	is_full = buffer_info.flags & BDR_OVERFLOWN_BUFFER_FLAG;
	
	if (!is_full) {
		rb->buffer_info.last = rb->buffer_info.offset;
		rb->buffer_info.offset = (rb->buffer_info.offset + rb->buffer_info.length) % rb->buffer_info.max_writes;
		rb->buffer_info.length = 0;
	}

	spin_unlock(&rb->lock);
	
	return buffer_info;
}

void bdr_ring_buffer_read(struct bdr_ring_buffer *rb) {
	spin_lock(&rb->lock);
	rb->buffer_info.last = rb->buffer_info.offset;
	rb->buffer_info.offset = (rb->buffer_info.offset + rb->buffer_info.length) % rb->buffer_info.max_writes;
	rb->buffer_info.length = 0;
	spin_unlock(&rb->lock);
}

int bdr_ring_buffer_put(struct bdr_ring_buffer *rb, unsigned int sector, unsigned int size, struct page *page, unsigned int page_offset, unsigned int length, struct bdr_bitmap *bitmap)
{
	struct bdr_buffer_info buffer_info;
	unsigned long buffer_offset;
	struct bdr_write_info *target_slot;
	void *page_addr;
	unsigned int copy_len;

	/* Acquire lock and update buffer state, has to be in one lock since weird things could happen */
	spin_lock(&rb->lock);
	
	if (rb->buffer_info.flags & BDR_OVERFLOWN_BUFFER_FLAG) {
		spin_unlock(&rb->lock);
		return -ENOSPC;
	}

	buffer_info = rb->buffer_info;
	bdr_ring_buffer_update_unsafe(rb);

	if (rb->buffer_info.flags & BDR_OVERFLOWN_BUFFER_FLAG) {
		spin_unlock(&rb->lock);
		return -ENOSPC;
	}

	spin_unlock(&rb->lock);

	/* Calculate buffer offset and wrap around as needed */
	buffer_offset = (buffer_info.offset + buffer_info.length) % rb->buffer_info.max_writes;
	buffer_offset *= BDR_WRITE_INFO_SIZE;

	/* Prepare target location in the ring buffer */
	target_slot = (struct bdr_write_info *)(rb->buffer + buffer_offset);

	target_slot->sector = sector;
	target_slot->size = size;

	/* Calculate length to copy and map page for read */
	copy_len = min(length, PAGE_SIZE - page_offset);
	page_addr = kmap_local_page(page);

	/* Copy the specified data into the ring buffer and unmap page */
	memcpy(target_slot->data, page_addr + page_offset, copy_len);
	kunmap_local(page_addr);

	return 0;
}

unsigned long bdr_ring_buffer_get_byte_size(struct bdr_ring_buffer *rb)
{
	return BDR_WRITES_TO_BYTES(rb->buffer_info.max_writes);
}

void *bdr_ring_buffer_get_buffer(struct bdr_ring_buffer *rb)
{
	return rb->buffer;
}
