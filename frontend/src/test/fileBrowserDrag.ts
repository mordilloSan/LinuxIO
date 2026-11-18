/**
 * The payload a drag started on this page carries, token and all, for tests
 * that drop it on a target: pass a call of setInternalDrag or setTrashDrag.
 *
 *   dragMadeHere((dataTransfer) => setInternalDrag(dataTransfer, ["/srv/a"]))
 */
export function dragMadeHere(
  start: (dataTransfer: DataTransfer) => void,
): unknown {
  let json = "";
  start({
    effectAllowed: "",
    setData: (_type: string, value: string) => {
      json = value;
    },
  } as unknown as DataTransfer);
  return JSON.parse(json);
}
