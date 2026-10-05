export function useUser(): string {
  return "u";
}

export function UserCard(props: { name: string }) {
  return <span>{props.name}</span>;
}

export const Layout = {
  Header: () => <header />,
};
