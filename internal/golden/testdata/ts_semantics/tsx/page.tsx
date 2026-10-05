import { UserCard, useUser, Layout } from "./card";

export function UserPage() {
  const name = useUser();
  return (
    <div>
      <Layout.Header />
      <UserCard name={name} />
      <button />
    </div>
  );
}
