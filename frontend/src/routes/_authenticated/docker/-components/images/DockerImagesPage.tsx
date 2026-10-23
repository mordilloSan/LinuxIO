import ImageList from "./ImageList";
import DockerResourcePage from "../shared/DockerResourcePage";

const IMAGE_PRUNE_OPTIONS = {
  buildCache: false,
  containers: false,
  images: true,
  networks: false,
  volumes: false,
} as const;

const DockerImagesPage = () => (
  <DockerResourcePage
    label="Image"
    List={ImageList}
    pruneOptions={IMAGE_PRUNE_OPTIONS}
    viewModeKey="docker.images"
  />
);

export default DockerImagesPage;
