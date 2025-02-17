#ifndef BDR_MAIN_H
#define BDR_MAIN_H

#include "ring-buffer.h"

#define BDR_MAX_DEVICES 64

/*
 * context for bdr
 */
struct bdr_context {
	/* pointer to underlying device */
	struct dm_dev *dev;

	/* buffer to which we put writes to */
	struct bdr_ring_buffer ring_buf;

	/* numbers for the chardev */
	dev_t chardev_num;

	/* actual chardev struct */
	struct cdev chardev;

	/* character device name */
	char* chardev_name;

	enum  {
		ACTIVE,
		OVERFLOWING,
		STOPPED,
	} status;

};

#endif
