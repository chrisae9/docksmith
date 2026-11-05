export type ContainerSortBy = 'name' | 'status' | 'created' | 'image';
export type ContainerGroupBy = 'stack' | 'none' | 'status' | 'image' | 'network';
export type ImageSortBy = 'tags' | 'size' | 'created' | 'id';
export type ImageGroupBy = 'none' | 'repository';
export type NetworkSortBy = 'name' | 'driver' | 'containers' | 'created';
export type NetworkGroupBy = 'none' | 'driver';
export type VolumeSortBy = 'name' | 'size' | 'created' | 'driver';
export type VolumeGroupBy = 'none' | 'type';

export interface ExplorerSettings {
  containers: {
    sortBy: ContainerSortBy;
    groupBy: ContainerGroupBy;
    ascending: boolean;
    showLabels: boolean;
    showRunningOnly: boolean;
  };
  images: {
    sortBy: ImageSortBy;
    groupBy: ImageGroupBy;
    ascending: boolean;
    showDanglingOnly: boolean;
  };
  networks: {
    sortBy: NetworkSortBy;
    groupBy: NetworkGroupBy;
    ascending: boolean;
    hideBuiltIn: boolean;
  };
  volumes: {
    sortBy: VolumeSortBy;
    groupBy: VolumeGroupBy;
    ascending: boolean;
    showUnusedOnly: boolean;
  };
}

export const DEFAULT_SETTINGS: ExplorerSettings = {
  containers: {
    sortBy: 'name',
    groupBy: 'stack',
    ascending: true,
    showLabels: false,
    showRunningOnly: false,
  },
  images: {
    sortBy: 'tags',
    groupBy: 'repository',
    ascending: true,
    showDanglingOnly: false,
  },
  networks: {
    sortBy: 'name',
    groupBy: 'none',
    ascending: true,
    hideBuiltIn: false,
  },
  volumes: {
    sortBy: 'name',
    groupBy: 'type',
    ascending: true,
    showUnusedOnly: false,
  },
};

