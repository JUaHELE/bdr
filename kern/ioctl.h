#ifndef BDR_IOCTL_H
#define BDR_IOCTL_H

#include "main.h"
#include "ring-buffer.h"

/*
 * serves to let the userspace know necessary constants
 */
struct bdr_target_info {
	u64 page_size;
	u32 write_info_size;
	u64 buffer_byte_size;
};

struct bdr_target_info bdr_get_target_info(struct bdr_ring_buffer *rb);

// IOCTL CONSTANTS
#define BDR_MAGIC 'B'
#define BDR_CMD_GET_BUFFER_INFO _IOR(BDR_MAGIC, 1, struct bdr_buffer_info)
#define BDR_CMD_GET_TARGET_INFO _IOR(BDR_MAGIC, 2, struct bdr_target_info)
#define BDR_CMD_GET_STATUS _IOR(BDR_MAGIC, 3, enum bdr_status)
#define BDR_CMD_GET_BUFFER_INFO_WAIT _IOR(BDR_MAGIC, 4, struct bdr_buffer_info)

#endif
