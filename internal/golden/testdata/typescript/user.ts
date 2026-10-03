export interface Account {
  id: number;
  name: string;
}

export class User implements Account {
  id = 0;
  name = "";

  save(): void {}
}

export const DEFAULT_ROLE = "member";
