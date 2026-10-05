import { User as DomainUser } from "../domain";

export function makeUser(name: string): DomainUser {
  return new DomainUser(name);
}
