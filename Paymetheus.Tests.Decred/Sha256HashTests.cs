// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Decred
{
    public static class Sha256HashTests
    {
        [Fact]
        public static void HasReferenceSemantics()
        {
            var firstArray = new byte[Blake256Hash.Length];
            var secondArray = new byte[Blake256Hash.Length];

            for (var i = 0; i < Blake256Hash.Length; i++)
            {
                firstArray[i] = (byte)i;
                secondArray[i] = (byte)i;
            }

            var firstHash = new Blake256Hash(firstArray);
            var secondHash = new Blake256Hash(secondArray);

            // Value equality
            Assert.Equal(firstHash, secondHash);
            Assert.True(firstHash.Equals(secondHash));
            Assert.Equal(firstHash.GetHashCode(), secondHash.GetHashCode());

            // Reference equality
            Assert.False(firstHash == secondHash);

            var hashSet = new HashSet<Blake256Hash> { firstHash };
            Assert.True(hashSet.Contains(secondHash));
        }
    }
}
