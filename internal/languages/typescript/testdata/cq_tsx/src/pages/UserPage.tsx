import { UserCard } from "../components";
import { useUser } from "../hooks/useUser";
import * as UI from "../ui/Button";

export function UserPage() {
  const user = useUser();
  return (
    <div>
      <UserCard user={user} />
      <UI.Button />
      <span />
    </div>
  );
}

export const Home = () => <UserPage />;
