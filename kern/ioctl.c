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
#include <linux/ioctl.h>

#include "ioctl.h"
#include "bitmap.h"

struct bdr_target_info bdr_get_target_info(struct bdr_ring_buffer *rb, struct bdr_bitmap *bm) {
	struct bdr_target_info target_info;

	target_info.page_size = PAGE_SIZE;
	target_info.write_info_size = BDR_WRITE_INFO_SIZE;
	target_info.buffer_byte_size = bdr_ring_buffer_get_byte_size(rb);
	target_info.bitmap_byte_size = bdr_bitmap_get_byte_size(bm);

	return target_info;
}

