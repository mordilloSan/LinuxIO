import React from "react";

const FilebrowserIframe: React.FC = () => {
  return (
    <iframe
      src="/navigator"
      style={{
        width: "100%",
        height: "90vh",
        border: "none",
        borderRadius: "8px",
        background: "#1e1e1e", // Optional: helps with dark backgrounds
      }}
      title="FileBrowser"
      allow="fullscreen"
    />
  );
};

export default FilebrowserIframe;
