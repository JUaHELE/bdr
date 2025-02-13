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

struct bdr_target_info bdr_get_target_info(struct bdr_ring_buffer *rb) {
	struct bdr_target_info module_info;

	module_info.page_size = PAGE_SIZE;
	module_info.write_info_size = BDR_WRITE_INFO_SIZE;
	module_info.buffer_byte_size = bdr_ring_buffer_get_byte_size(rb);

	return module_info;
}

