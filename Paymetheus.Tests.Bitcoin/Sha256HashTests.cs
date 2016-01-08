// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using System.Collections.Generic;
using Xunit;

namespace Paymetheus.Tests.Bitcoin
{
    public static class Sha256HashTests
    {
        [Fact]
        public static void HasReferenceSemantics()
        {
            var firstArray = new byte[Sha256Hash.Length];
            var secondArray = new byte[Sha256Hash.Length];

            for (var i = 0; i < Sha256Hash.Length; i++)
            {
                firstArray[i] = (byte)i;
                secondArray[i] = (byte)i;
            }

            var firstHash = new Sha256Hash(firstArray);
            var secondHash = new Sha256Hash(secondArray);

            // Value equality
            Assert.Equal(firstHash, secondHash);
            Assert.True(firstHash.Equals(secondHash));
            Assert.Equal(firstHash.GetHashCode(), secondHash.GetHashCode());

            // Reference equality
            Assert.False(firstHash == secondHash);

            var hashSet = new HashSet<Sha256Hash> { firstHash };
            Assert.True(hashSet.Contains(secondHash));
        }
    }
}
