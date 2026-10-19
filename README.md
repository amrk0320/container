# Mini Container Runtime

A lightweight container runtime implemented in Go from scratch. This project demonstrates the core internal mechanisms of container engines like Docker (runc), utilizing Linux kernel features to create isolated environments.

## Core Features

- **Namespaces (`CLONE_NEW*`)**: Isolates UTS (hostname), PID (process trees), NS (mount points), and NET (network interfaces) to create a fully sandboxed environment.
- **Cgroups**: Limits resource usage. Currently prevents fork bombs by restricting the maximum number of child processes to 20 (`pids.max`).
- **Chroot**: Jails the container process into a specific root filesystem.
- **OverlayFS**: Implements a union mount filesystem. Containers share a single read-only base image (`lowerdir`) while writing all modifications to a container-specific ephemeral layer (`upperdir`). This enables launching multiple independent containers instantaneously from the same base image.

## Prerequisites

- [Docker](https://www.docker.com/) (Docker Desktop, OrbStack, or Colima on macOS/Windows; Docker Engine on Linux).

Since macOS/Windows does not have native Linux namespaces, this runtime runs inside a privileged Linux container provided by Docker.

## Setup 1: Start the development container

Run this from the repository root (the directory containing `main.go`):

```bash
docker run -it --name mini-container --privileged \
  -v "$(pwd)":/work -w /work \
  -v container-rootfs:/home/ubuntu/rootfs \
  -v container-layers:/home/ubuntu/containers \
  golang:1.26 bash
```

- `--privileged` is required to create namespaces, mount `/proc`, and mount OverlayFS inside the container.
- The source code is bind-mounted at `/work`.
- The base image (`rootfs`) and the container layers are stored in **Docker named volumes**, not in a folder shared with macOS/Windows. Extracting the rootfs on a shared folder breaks symbolic links, and OverlayFS cannot use the container's own root filesystem as its upper layer.

*(Note: All subsequent commands must be executed inside this container shell.)*

## Setup 2: Prepare the Base Image (rootfs)

Before running a container, you need a base Linux filesystem. We will download a minimal Alpine Linux rootfs that matches your CPU architecture (`x86_64` or `aarch64`).

```bash
ARCH=$(uname -m)
cd /home/ubuntu/rootfs
wget https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/${ARCH}/alpine-minirootfs-3.18.4-${ARCH}.tar.gz
tar -xf alpine-minirootfs-3.18.4-${ARCH}.tar.gz
rm alpine-minirootfs-3.18.4-${ARCH}.tar.gz
cd /work
```

Check that the shell exists:

```bash
ls /home/ubuntu/rootfs/bin/sh
```

The volume keeps the rootfs, so you only need to do this once.

## Setup 3: Enable the pids cgroup controller

Inside Docker, the processes already running in the container's cgroup prevent enabling controllers for child cgroups. Move them into a sub-cgroup first, then enable the controllers:

```bash
mkdir -p /sys/fs/cgroup/init
xargs -rn1 < /sys/fs/cgroup/cgroup.procs > /sys/fs/cgroup/init/cgroup.procs || :
sed -e 's/ / +/g' -e 's/^/+/' < /sys/fs/cgroup/cgroup.controllers > /sys/fs/cgroup/cgroup.subtree_control
```

Run this once each time the Docker container (re)starts. Without it, `pids.max` is not created and the process limit is silently not applied.

## Build & Run

1. Build the Go binary:

```bash
go build -o container main.go
```

2. Run a container with a specific name and start the shell:

```bash
./container run app1 /bin/sh
```

(You are already root inside the Docker container, so `sudo` is not needed.)

3. Run multiple containers simultaneously:

Open another terminal on your host, enter the same Docker container, and run:

```bash
docker exec -it mini-container bash
./container run app2 /bin/sh
```

Both `app1` and `app2` will run in complete isolation sharing the same base `rootfs` via OverlayFS.

4. Verify the process limit (from the Docker container shell, not inside `app1`):

```bash
cat /sys/fs/cgroup/app1/pids.max
```

It should print `20`.

