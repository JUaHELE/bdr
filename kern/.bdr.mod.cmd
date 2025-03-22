savedcmd_bdr.mod := printf '%s\n'   main.o ring-buffer.o bitmap.o ioctl.o | awk '!x[$$0]++ { print("./"$$0) }' > bdr.mod
