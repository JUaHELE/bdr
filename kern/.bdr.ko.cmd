savedcmd_bdr.ko := ld -r -m elf_x86_64 -z noexecstack --no-warn-rwx-segments --build-id=sha1  -T /usr/src/kernels/6.13.7-200.fc41.x86_64/scripts/module.lds -o bdr.ko bdr.o bdr.mod.o .module-common.o
