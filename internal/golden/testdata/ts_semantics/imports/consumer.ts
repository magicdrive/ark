import makeUser, { User, User as DomainUser } from "./user";
import * as users from "./user";
import type { UserId } from "./user";
import { z } from "zod";
import { helper } from "@/shared/helper";

export function build(id: UserId): DomainUser {
  const a = makeUser("a");
  const b = users.User.create("b");
  const c = new DomainUser("c");
  z.string();
  helper();
  return User.create(String(id));
}
