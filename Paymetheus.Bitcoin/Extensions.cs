// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Collections.Generic;

namespace Paymetheus.Bitcoin
{
    public static class Extensions
    {
        public static IEnumerable<T> ReverseList<T>(this IList<T> list)
        {
            if (list == null)
                yield break;

            for (var i = list.Count - 1; i >= 0; i--)
                yield return list[i];
        }
    }
}
