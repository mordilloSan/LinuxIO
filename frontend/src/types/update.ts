export interface Update {
  changelog: string;
  cve: string[];
  issued: string;
  package_id: string;
  restart: number;
  state: number;
  summary: string;
  version: string;
}
