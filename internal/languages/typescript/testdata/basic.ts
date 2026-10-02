import { EventEmitter } from "events";
import type { Readable } from "stream";

export interface Repository {
  save(entity: User): Promise<void>;
  findById(id: string): Promise<User | null>;
}

export type UserID = string;

export class UserService {
  private repo: Repository;

  constructor(repo: Repository) {
    this.repo = repo;
  }

  async create(name: string): Promise<User> {
    const user = new User(name);
    await this.repo.save(user);
    return user;
  }
}

export class User {
  id: UserID;
  name: string;

  constructor(name: string) {
    this.id = crypto.randomUUID();
    this.name = name;
  }
}

export function formatUser(u: User): string {
  return `${u.name} (${u.id})`;
}

export const DEFAULT_TIMEOUT = 5000;
