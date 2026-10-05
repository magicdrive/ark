import type { User } from "../domain/user";

export interface UserCardProps {
  user: User;
}

export function UserCard(props: UserCardProps) {
  return <span>{props.user.name}</span>;
}
