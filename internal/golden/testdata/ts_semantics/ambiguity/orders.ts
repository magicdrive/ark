import { UserRepository } from "./repositories";

export class OrderRepository {
  save(o: unknown): void {}
}

export function persist(repo: UserRepository, x: unknown): void {
  repo.save(x);
}

export function unknownReceiver(r: any): void {
  r.save(1);
}
