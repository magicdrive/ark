export class User {
  constructor(public readonly name: string) {}

  greet(): string {
    return `hi ${this.name}`;
  }
}

export type UserId = string;
