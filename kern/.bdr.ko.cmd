savedcmd_bdr.ko := ld -r -m elf_x86_64 -z noexecstack --no-warn-rwx-segments --build-id=sha1  -T /usr/lib/modules/6.13.8-arch1-1/build/scripts/module.lds -o bdr.ko bdr.o bdr.mod.o .module-common.o
