import "./page-loader.css";

function PageLoader() {
  return (
    <div className="page-loader">
      <div className="page-loader__dots">
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
        <span className="page-loader__dot" />
      </div>
    </div>
  );
}

export default PageLoader;
