package ebpf

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64,s390x -type block_hist_key -type hist block ../../bpf/block_latency.c -- -I../../bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64,s390x -type nfs_hist_key -type hist nfs ../../bpf/nfs_latency.c -- -I../../bpf/headers
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" -target amd64,arm64,s390x nfsKprobe ../../bpf/nfs_kprobe_latency.c -- -I../../bpf/headers
