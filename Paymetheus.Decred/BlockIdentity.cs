// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Decred
{
    public struct BlockIdentity : IEquatable<BlockIdentity>
    {
        public static readonly BlockIdentity Unmined = new BlockIdentity(Blake256Hash.Zero, -1);

        public BlockIdentity(Blake256Hash hash, int height)
        {
            Hash = hash;
            Height = height;
        }

        public Blake256Hash Hash { get; }
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