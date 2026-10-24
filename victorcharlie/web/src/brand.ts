// The name people see. The server sends its configured app_name; this
// default covers the moment before that arrives.
let name = "Project Victor Charlie";

export const appName = () => name;

export function setAppName(n: string) {
  if (n) name = n;
  document.title = name;
}
