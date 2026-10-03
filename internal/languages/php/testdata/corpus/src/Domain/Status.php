<?php
namespace App\Domain;

enum Status: string
{
    case Active = 'active';
    case Disabled = 'disabled';

    public function label(): string
    {
        return match ($this) {
            Status::Active => 'Active',
            Status::Disabled => 'Disabled',
        };
    }
}
