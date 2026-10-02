import { EventEmitter } from "events";

export class UserService {
  constructor(repo) {
    this.repo = repo;
  }

  async create(name) {
    const user = new User(name);
    await this.repo.save(user);
    return user;
  }
}

export class User {
  constructor(name) {
    this.id = Math.random().toString(36);
    this.name = name;
  }
}

export function formatUser(u) {
  return `${u.name} (${u.id})`;
}

export const DEFAULT_TIMEOUT = 5000;
