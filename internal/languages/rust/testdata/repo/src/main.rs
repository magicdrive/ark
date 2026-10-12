use app::storage::Repository;

fn main() {
    let mut repo = Repository::new();
    run(&mut repo);
}

fn run(_repo: &mut Repository) {
    helper();
}

fn helper() {}
