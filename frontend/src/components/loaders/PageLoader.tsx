interface PageLoaderProps {
  /** What is loading, read out by screen readers. */
  label?: string;
}

function PageLoader({ label = "Loading" }: PageLoaderProps) {
  return (
    <div className="page-loader" role="status" aria-label={label}>
      <div className="page-loader__dots" aria-hidden="true">
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
      </div>
    </div>
  );
}

export default PageLoader;
