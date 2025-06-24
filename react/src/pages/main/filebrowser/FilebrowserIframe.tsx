import React from "react";

const FilebrowserIframe: React.FC = () => {
  return (
    <iframe
      src="/navigator"
      style={{
        width: "100%",
        height: "90vh",
        border: "none",
      }}
      title="FileBrowser"
      allow="fullscreen"
    />
  );
};

export default FilebrowserIframe;
