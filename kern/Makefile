obj-m += bdr.o
bdr-objs := \
	main.o ring-buffer.o bitmap.o ioctl.o

MODULEDIR := /lib/modules/$(shell uname -r)
KERNELDIR := $(MODULEDIR)/build

SOURCES := $(wildcard *.c)
HEADERS := $(wildcard *.h)

build: bdr.ko

bdr.ko: $(SOURCES) $(HEADERS)
	make -C $(KERNELDIR) M=$(shell pwd) modules

clean:
	make -C $(KERNELDIR) M=$(shell pwd) clean

