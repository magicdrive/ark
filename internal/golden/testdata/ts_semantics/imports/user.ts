export class User {
  constructor(public name: string) {}
  static create(name: string): User {
    return new User(name);
  }
}

export default function makeUser(name: string): User {
  return User.create(name);
}

export type UserId = string;
