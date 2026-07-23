function PageLoader() {
  return (
    <div className="page-loader" role="status" aria-label="Loading">
      <div className="page-loader__dots" aria-hidden="true">
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
      </div>
    </div>
  );
}

export default PageLoader;
