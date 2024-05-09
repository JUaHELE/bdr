#include <linux/kernel.h>
#include <linux/module.h>

struct class *bdr_cclass;

static int __init bdr_init(void)
{
	int r;

	bdr_cclass = class_create(THIS_MODULE, "bdrc");
	if (IS_ERR(bdr_cclass))
		return r;

	return 0;
}

static void __exit bdr_exit(void)
{
	class_destroy(bdr_cclass);
}

module_init(bdr_init);
module_exit(bdr_exit);

MODULE_LICENSE("GPL");
MODULE_AUTHOR("Hynek Havel <hynek2002@gmail.com>");
MODULE_DESCRIPTION("BDR");
MODULE_VERSION("0.0.1");
