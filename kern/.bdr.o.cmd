savedcmd_bdr.o := ld -m elf_x86_64 -z noexecstack --no-warn-rwx-segments   -r -o bdr.o @bdr.mod  ; /usr/lib/modules/6.13.8-arch1-1/build/tools/objtool/objtool --hacks=jump_label --hacks=noinstr --hacks=skylake --ibt --orc --retpoline --rethunk --sls --static-call --uaccess --prefix=16  --link  --module bdr.o

bdr.o: $(wildcard /usr/lib/modules/6.13.8-arch1-1/build/tools/objtool/objtool)
