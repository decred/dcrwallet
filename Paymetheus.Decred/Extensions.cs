// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Decred
{
    public static class Extensions
    {
        public static IEnumerable<T> ReverseList<T>(this IList<T> list)
        {
            if (list == null)
                throw new ArgumentNullException(nameof(list));

            for (var i = list.Count - 1; i >= 0; i--)
                yield return list[i];
        }
    }
}
