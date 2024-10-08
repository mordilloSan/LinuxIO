// Third-party Imports
import classnames from "classnames";

// Util Imports
import { blankLayoutClasses } from "../../../utils/layoutClasses";

const BlankLayout = (props) => {
  // Props
  const { children } = props;

  return (
    <div className={classnames(blankLayoutClasses.root, "is-full bs-full")}>
      {children}
    </div>
  );
};

export default BlankLayout;
