// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Bitcoin
{
    public struct BlockIdentity : IEquatable<BlockIdentity>
    {
        public static readonly BlockIdentity Unmined = new BlockIdentity(Sha256Hash.Zero, -1);

        public BlockIdentity(Sha256Hash hash, int height)
        {
            Hash = hash;
            Height = height;
        }

        public Sha256Hash Hash { get; }
        public int Height { get; }

        public bool IsUnmined() => Height == Unmined.Height;

        public static bool operator ==(BlockIdentity lhs, BlockIdentity rhs) =>
            lhs.Height == rhs.Height && lhs.Hash.Equals(rhs.Hash);

        public static bool operator !=(BlockIdentity lhs, BlockIdentity rhs) => !(lhs == rhs);

        public bool Equals(BlockIdentity other) => this == other;

        public override bool Equals(object obj) => obj is BlockIdentity && this == (BlockIdentity)obj;

        public override int GetHashCode() => Height.GetHashCode() ^ Hash.GetHashCode();
    }
}