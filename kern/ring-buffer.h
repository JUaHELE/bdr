#ifndef BDR_RING_BUFFER_H
#define BDR_RING_BUFFER_H

/*
 * This header implements a ring buffer and differs a bit to suite my needs
 * bdr_ring_buffer loops around the within the buffer as normal circular buffer when putting
 * but whenever read is performed all new writes are understood as invalid
 * read is performed by returning information about the buffer since userspace will read it
 */

#define BDR_OVERFLOWN_BUFFER_FLAG	BIT(0)

/*
 * holds information about one singular write
 * these structs are filled into the shared buffer
 */
struct bdr_write_info {
	u32 sector;
	u32 size;
	char data[PAGE_SIZE];
};

#define BDR_WRITE_INFO_SIZE sizeof(struct bdr_write_info)
#define BDR_WRITES_TO_BYTES(n) (n * BDR_WRITE_INFO_SIZE)

struct bdr_buffer_stats {
	atomic_t total_writes;
	atomic_t overflow_count;
	atomic_t total_reads;
};

/*
 * holds information about new writes in the buffer
 */
struct bdr_buffer_info {
	/* offset to buffer where the new writes start */
	u64 offset;

	/* count of new writes in the buffer */
	u64 length;

	/* offset to last write that userspace program has taken care of */
	u64 last;

	/* specify the actual state of the buffer */
	u32 flags;
	
	/* how many writes fit into the shared buffer (one write is described by one struct bdr_write_info)*/
	u64 max_writes;
};

struct bdr_ring_buffer {
	/* pointer to shared buffer which will be exposed between kernel and userspace */
	void* buffer;

	/* information about new writes in buffer */
	struct bdr_buffer_info buffer_info;

	spinlock_t lock;
};

/*
 * initiates and allocates resources
 * size is a number which describes how many actual writes fit into the buffer
 */
int bdr_ring_buffer_init(struct bdr_ring_buffer *rb, u32 max_writes);

/*
 * frees alocated resources within ring buffer
 */
void bdr_ring_buffer_free(struct bdr_ring_buffer *rb);

bool bdr_ring_buffer_is_empty(struct bdr_ring_buffer *rb);

/*
 * gets information from the ring buffer
 */
struct bdr_buffer_info
bdr_ring_buffer_get_info(struct bdr_ring_buffer *rb);

/*
 * checks if buffer overflown
 */
bool bdr_ring_buffer_is_full(struct bdr_ring_buffer *rb);

/*
 * increments lenght of the buffer by 1 and checks overflow
 */
void bdr_ring_buffer_update_unsafe(struct bdr_ring_buffer *rb);

/*
 * resets buffer information
 */
void bdr_ring_buffer_reset(struct bdr_ring_buffer *rb);

/*
 * read from buffer - new writes are expecting to be read
 */
void bdr_ring_buffer_read(struct bdr_ring_buffer *rb);

/*
 * routine for updating the buffer after userspace reads from it
 */
struct bdr_buffer_info
bdr_buffer_read_routine(struct bdr_ring_buffer *rb);

/*
 * puts data into the buffer
 */
int bdr_ring_buffer_put(struct bdr_ring_buffer *rb, unsigned int sector, unsigned int size, struct page *page, unsigned int page_offset, unsigned int length);

/*
 * get size from the buffer
 */
unsigned long bdr_ring_buffer_get_byte_size(struct bdr_ring_buffer *rb);

/*
 * retreives pointer to buffer
 */
void *bdr_ring_buffer_get_buffer(struct bdr_ring_buffer *rb);
#endif
