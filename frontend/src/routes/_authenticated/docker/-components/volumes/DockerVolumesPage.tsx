import VolumeList from "./VolumeList";
import DockerResourcePage from "../shared/DockerResourcePage";

const VOLUME_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: false,
  networks: false,
  volumes: true,
} as const;

const DockerVolumesPage = () => (
  <DockerResourcePage
    label="Volume"
    List={VolumeList}
    pruneOptions={VOLUME_PRUNE_OPTIONS}
    viewModeKey="docker.volumes"
  />
);

export default DockerVolumesPage;
