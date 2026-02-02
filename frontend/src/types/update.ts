export interface Update {
  package_id: string;
  summary: string;
  version: string;
  issued: string;
  changelog: string;
  cve: string[];
  restart: number;
  state: number;
};
