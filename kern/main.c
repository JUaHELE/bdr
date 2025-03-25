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

#include "ring-buffer.h"
#include "bitmap.h"
#include "ioctl.h"
#include "main.h"

/* class for the chardev */
static struct class *bdr_chardev_class;

/* save major number for character devices */
static dev_t bdr_dev_major;

/* track existing character devices minor numbers */
static struct bdr_bitmap bdr_minor_bitmap;

/* if no new writes are available */
static DECLARE_WAIT_QUEUE_HEAD(bdr_wait_queue);

/*
 * mmaps ring buffer into the userspace
 */
static int bdr_chardev_mmap(struct file *filp, struct vm_area_struct *vma)
{
	struct bdr_context *bc = filp->private_data;
	int ret;

	if (!bc) {
		pr_err("No context found for mmap\n");
		return -EINVAL;
	}

	unsigned long rb_size = bdr_ring_buffer_get_byte_size(&bc->ring_buf);

	unsigned long mapped_size = vma->vm_end - vma->vm_start;
	if (mapped_size < rb_size) {
		pr_warn("Requested memory is not enough to cover the buffer\n");
		mapped_size = rb_size;
	}

	void *target_buffer = bdr_ring_buffer_get_buffer(&bc->ring_buf);
	if (!target_buffer) {
	    pr_err("Failed to get buffer for mmap\n");
	    return -EINVAL;
	}

	for (unsigned long offset = 0; offset < rb_size; offset += PAGE_SIZE) {
		void *page_address = target_buffer + offset;
		unsigned long pfn = vmalloc_to_pfn(page_address);

		ret = remap_pfn_range(vma,
				      vma->vm_start + offset,
				      pfn,
				      PAGE_SIZE,
				      vma->vm_page_prot);

		if (ret) {
			pr_err("Failed to map page at offset %lu\n", offset);
			return ret;
		}
	}
	return 0;
}

/*
 * open function for character device
 * just saves private content of target to character device private field
 */
static int bdr_chardev_open(struct inode *inode, struct file *filp)
{
	struct cdev *target_chardev = inode->i_cdev;

	struct bdr_context *bc = container_of(target_chardev, struct bdr_context, chardev);
	filp->private_data = bc;
	return 0;
}

/*
 * ioctl handler for character device
 */
static long bdr_chardev_ioctl(struct file *filp, unsigned int cmd, unsigned long arg)
{
	struct bdr_context *bc = filp->private_data;
	struct bdr_ring_buffer *rb = &bc->ring_buf;
	struct bdr_bitmap *mb = &bc->overflow_bm;
	struct bdr_buffer_info buffer_info;
	struct bdr_target_info target_info;
	enum bdr_status status;

	int ret = 0;

	switch(cmd) {
	case BDR_CMD_GET_BUFFER_INFO:
		buffer_info = bdr_ring_buffer_get_info(rb);
		if (copy_to_user((void __user *)arg, &buffer_info, sizeof(buffer_info))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_GET_TARGET_INFO:
		target_info = bdr_get_target_info(rb, mb);
		if (copy_to_user((void __user *)arg, &target_info, sizeof(target_info))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_GET_STATUS:
		status = bc->status;
		if (copy_to_user((void __user *)arg, &status, sizeof(status))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_GET_BUFFER_INFO_WAIT:
		wait_event_interruptible(bdr_wait_queue, rb->buffer_info.length != 0);

		buffer_info = bdr_ring_buffer_get_info(rb);
		if (copy_to_user((void __user *)arg, &buffer_info, sizeof(buffer_info))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_READ_BUFFER_INFO:
		buffer_info = bdr_buffer_read_routine(rb);
		if (copy_to_user((void __user *)arg, &buffer_info, sizeof(buffer_info))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_READ_BUFFER_INFO_WAIT:
		wait_event_interruptible(bdr_wait_queue, rb->buffer_info.length != 0);

		buffer_info = bdr_buffer_read_routine(rb);
		if (copy_to_user((void __user *)arg, &buffer_info, sizeof(buffer_info))) {
			ret = -EFAULT;
		}
		break;
	case BDR_CMD_RESET_BUFFER:
		bdr_ring_buffer_reset(rb);
		break;
	case BDR_CMD_WRITE_TEST_VALUE:
		uint32_t offset;
		if (copy_from_user(&offset, (void __user *)arg, sizeof(offset))) {
		    ret = -EFAULT;
		    break;
		}

		void *target_buffer = bdr_ring_buffer_get_buffer(&bc->ring_buf);
		if (!target_buffer) {
			pr_err("Failed to get buffer for test write\n");
			ret = -EINVAL;
			break;
		}

		((char *)target_buffer)[offset] = 0xAA;
		pr_info("Test value 0xAA written to mmaped buffer at offset: %iu\n", offset);
		break;
	default:
		pr_warn("Ioctl not recognized: cmd=%u\n", cmd);
		ret = -ENOTTY;
		break;
	}
	return ret;
}

static ssize_t bdr_chardev_read(struct file *filp, char __user *buf, 
                               size_t count, loff_t *f_pos)
{
	struct bdr_context *bc = filp->private_data;

	if (!bc) {
		pr_err("No context found for read\n");
		return -EINVAL;
	}

	unsigned long bitmap_size = bdr_bitmap_get_byte_size(&bc->overflow_bm);
	if (bitmap_size != count) {
		pr_err("Bitmap size and requested bytes to read do not equal.");
		return -EINVAL;
	}

	spin_lock(&bc->overflow_bm.lock);
	bdr_bitmap_print(&bc->overflow_bm);
	if (copy_to_user(buf, (char*)bc->overflow_bm.bitmap, bitmap_size)) {
		spin_unlock(&bc->overflow_bm.lock);
		return -EFAULT;
	}
	// reset the bitmap
	memset(bc->overflow_bm.bitmap, 0, BITS_TO_LONGS(bc->overflow_bm.max_bits) * sizeof(unsigned long));
	spin_unlock(&bc->overflow_bm.lock);

	return bitmap_size;
}

/*
 * fops for character device
 */
static struct file_operations bdr_chardev_fops = {
	.owner = THIS_MODULE,
	.mmap = bdr_chardev_mmap,
	.open = bdr_chardev_open,
	.read = bdr_chardev_read,
	.unlocked_ioctl = bdr_chardev_ioctl,
};

/*
 * 
 */
static int bdr_chardev_init(struct bdr_context *bc)
{
	int ret;

	int minor = bdr_bitmap_allocate(&bdr_minor_bitmap);
	if (minor < 0) {
		pr_err("No more minor numbers available for bdr devices\n");
		return -ENOMEM;
	}

	bc->chardev_num = MKDEV(MAJOR(bdr_dev_major), minor);

	struct device *dev = device_create(bdr_chardev_class, NULL, bc->chardev_num, NULL, bc->chardev_name);
	if (IS_ERR(dev)) {
		ret = PTR_ERR(dev);
		pr_err("Failed to create character device");
		goto err_device;
	}

	cdev_init(&bc->chardev, &bdr_chardev_fops);
	ret = cdev_add(&bc->chardev, bc->chardev_num, 1);
	if (ret < 0) {
		pr_err("Failed to add character device");
		goto err_add;
	}
	
	return 0;

err_add:
	device_destroy(bdr_chardev_class, bc->chardev_num);

err_device:
	bdr_bitmap_free(&bdr_minor_bitmap, minor);

	return ret;
}

/*
 * removes character device
 */
static void bdr_chardev_destroy(struct bdr_context *bc)
{
	bdr_bitmap_free(&bdr_minor_bitmap, MINOR(bc->chardev_num));

	device_destroy(bdr_chardev_class, bc->chardev_num);
	cdev_del(&bc->chardev);
	unregister_chrdev_region(bc->chardev_num, 1);
}

static void bdr_target_dtr(struct dm_target *ti)
{
	struct bdr_context *bc = (struct bdr_context*)ti->private;

	bdr_chardev_destroy(bc);

	dm_put_device(ti, bc->dev);

	bdr_ring_buffer_free(&bc->ring_buf);

	bdr_bitmap_destroy(&bc->overflow_bm);

	kfree(bc->chardev_name);
	kfree(bc);
}

/*
 * constructor for bdr target mapping
 */
static int bdr_target_ctr(struct dm_target *ti, unsigned int argc, char **argv)
{
	int ret;
	
	if (argc != 3) {
		ti->error = "Invalid argument count, provide device path and char dev name";
		return -EINVAL;
	}

	struct bdr_context *bc = kzalloc(sizeof(struct bdr_context), GFP_KERNEL);
	if (bc == NULL) {
		ti->error = "Cannot allocate bdr context";
		return -ENOMEM;
	}

	/* argv[0] is backing device */
	ret = dm_get_device(ti, argv[0], dm_table_get_mode(ti->table), &bc->dev);
	if (ret) {
		ti->error = "Device lookup failed";
		goto err_get_dev;
	}

	/* argv[1] is name of the character device */
	bc->chardev_name = kstrdup(argv[1], GFP_KERNEL);
	if (!bc->chardev_name) {
		ti->error = "Failed to copy target name";
		ret = -ENOMEM;
		goto err_name;
	}

	unsigned int max_writes;
	if (sscanf(argv[2], "%iu", &max_writes) != 1 || max_writes == 0) {
		ti->error = "Invalid maximum of writes";
		goto err_scanf_buffer;
	}

	ret = bdr_ring_buffer_init(&bc->ring_buf, max_writes);
	if (ret) {
		ti->error = "Failed to initilize ring buffer";
		goto err_scanf_buffer;
	}

	/* initilize character device associated with the target */
	ret = bdr_chardev_init(bc);
	if (ret) {
		goto err_chardev;
	}

	sector_t device_size = get_capacity(bc->dev->bdev->bd_disk); // we get size in 512 bytes
	unsigned long num_blocks = device_size >> BDR_BLOCK_SIZE_SHIFT;  // Divide by 2048 sectors to get 1M blocks
	ret = bdr_bitmap_init(&bc->overflow_bm, num_blocks);
	if (ret) {
		goto err_bitmap;
	}

	ti->private = bc;

	/* once the target loads it starts replicating immediately */
	bc->status = ACTIVE;

	return 0;

err_bitmap:
	bdr_chardev_destroy(bc);

err_chardev:
	bdr_ring_buffer_free(&bc->ring_buf);

err_scanf_buffer:
	kfree(bc->chardev_name);

err_name:
	dm_put_device(ti, bc->dev);

err_get_dev:
	kfree(bc);
	return ret;
}

/*
 * copies write info to buffer
 */
static void bdr_put_write_to_buffer(struct bdr_ring_buffer *rb, struct bio *bio, struct bio_vec *bvec, struct bvec_iter *iter, struct bdr_bitmap *bitmap) {
	unsigned int seg_len = bvec->bv_len;
	unsigned int seg_page_off = bvec->bv_offset;
	struct page *seg_page = bvec->bv_page;
	unsigned int seg_sec = iter->bi_sector;

	while (seg_len > 0) {
		unsigned int size_to_copy = min(seg_len, PAGE_SIZE - seg_page_off);
		
		int ret = bdr_ring_buffer_put(rb, seg_sec, size_to_copy, seg_page, seg_page_off, seg_len, bitmap);
		if(ret)
			return;

		seg_len -= size_to_copy;
		seg_page_off = 0;
		seg_sec += 1;
	}
}

/*
 * puts writes into the buffer
 */
static void bdr_submit_bio(struct bdr_ring_buffer *rb, struct bio *bio, struct bdr_bitmap *bitmap)
{
	struct bio_vec bvec;
	struct bvec_iter iter;

	/* iterate through bvecs and send them to userspace */
	bio_for_each_segment(bvec, bio, iter) {
		/* write the page to shared buffer */
		bdr_put_write_to_buffer(rb, bio, &bvec, &iter, bitmap);
	}

	wake_up_interruptible(&bdr_wait_queue);
}


static int bdr_target_map(struct dm_target *ti, struct bio *bio)
{
	struct bdr_context *bc = (struct bdr_context*)ti->private;

	bool is_write;
	bool buffer_overflow;

	is_write = op_is_write(bio_op(bio));
	buffer_overflow = bdr_ring_buffer_is_full(&bc->ring_buf);

	if(is_write) {
		if (buffer_overflow) {
			struct bio_vec bvec;
			struct bvec_iter iter;

			/* iterate through bvecs and send them to userspace */
			bio_for_each_segment(bvec, bio, iter) {
				bdr_bitmap_set_sector(&bc->overflow_bm, iter.bi_sector);
			}
		} else {
			bdr_submit_bio(&bc->ring_buf, bio, &bc->overflow_bm);
		}
	}

	/* change device to underlying */
	bio_set_dev(bio, bc->dev->bdev);

	/* calculates the new sector offset within the underlying device */
	bio->bi_iter.bi_sector = dm_target_offset(ti, bio->bi_iter.bi_sector);

	/* finally the submit itself */
	submit_bio(bio);

	/* return DM_MAPIO_SUBMITTED since we submitted the bio for further process */
	return DM_MAPIO_SUBMITTED;
}


struct target_type bdr_target_fops = {
	.name = "bdr",
	.version = {0,0,2},
	.module = THIS_MODULE,
	.ctr = bdr_target_ctr,
	.dtr = bdr_target_dtr,
	.map = bdr_target_map,
};

/*
 * function invoked when the target is loaded
 */
static int __init bdr_init(void)
{
	int ret;

	ret = dm_register_target(&bdr_target_fops);
	if (ret) {
		return ret;
	}

	bdr_chardev_class = class_create("bdr_class");
	if (IS_ERR(bdr_chardev_class)) {
		ret = PTR_ERR(bdr_chardev_class);
		goto err_class;
	}

	ret = alloc_chrdev_region(&bdr_dev_major, 0, BDR_MAX_DEVICES, "bdr_region");
	if (ret < 0) {
		pr_err("Failed to allocate region for character devices");
		goto err_region;
	}

	ret = bdr_bitmap_init(&bdr_minor_bitmap, BDR_MAX_DEVICES);
	if (ret) {
		pr_err("Failed to initilize bitmap");
		goto err_bitmap;
	}
	return 0;

err_bitmap:
	unregister_chrdev_region(bdr_dev_major, BDR_MAX_DEVICES);

err_region:
	class_destroy(bdr_chardev_class);

err_class:
	dm_unregister_target(&bdr_target_fops);

	return ret;
}


static void __exit bdr_exit(void)
{
	class_destroy(bdr_chardev_class);
	dm_unregister_target(&bdr_target_fops);
	unregister_chrdev_region(bdr_dev_major, BDR_MAX_DEVICES);
}

module_init(bdr_init);
module_exit(bdr_exit);

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Hynek Havel <hynek2002@gmail.com>");
MODULE_DESCRIPTION("BDR");
MODULE_VERSION("0.0.2");
