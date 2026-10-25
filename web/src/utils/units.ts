// Human-unit <-> raw-API-unit conversions. The API/DB always use millicores
// for CPU and bytes for memory/storage (matching how K8s ResourceList is
// built in the backend's provisioner) — these just convert at the UI
// boundary so forms/tables can show cores/MB/GB instead. Binary base (1 MB =
// 1024^2 bytes, 1 GB = 1024^3 bytes), matching the binary math already used
// elsewhere in this app (e.g. workspace memory displayed as Gi).
export function millicoresToCores(millicores: number): number {
  return millicores / 1000
}

export function coresToMillicores(cores: number): number {
  return Math.round(cores * 1000)
}

export function bytesToMB(bytes: number): number {
  return bytes / 1024 ** 2
}

export function mbToBytes(mb: number): number {
  return Math.round(mb * 1024 ** 2)
}

export function bytesToGB(bytes: number): number {
  return bytes / 1024 ** 3
}

export function gbToBytes(gb: number): number {
  return Math.round(gb * 1024 ** 3)
}
