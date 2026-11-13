import DockerResourcePage from "../shared/DockerResourcePage";
import NetworkList from "./NetworkList";

const NETWORK_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: false,
  networks: true,
  volumes: false,
} as const;

const DockerNetworksPage = () => (
  <DockerResourcePage
    label="Network"
    List={NetworkList}
    pruneOptions={NETWORK_PRUNE_OPTIONS}
    viewModeKey="docker.networks"
  />
);

export default DockerNetworksPage;
