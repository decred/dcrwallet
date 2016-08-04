using System;
using System.Collections.Generic;
using System.Linq;
using System.Runtime.CompilerServices;
using System.Text;
using System.Threading.Tasks;

namespace Paymetheus.Framework
{
    public static class Messenger
    {
        public delegate void MessageHandler(IViewModelMessage message);

        static Dictionary<Type, MessageHandler> Instances = new Dictionary<Type, MessageHandler>();

        public static void RegisterSingleton<T>(MessageHandler handler) where T : ViewModelBase
        {
            Instances[typeof(T)] = handler;
        }

        public static void MessageSingleton<T>(IViewModelMessage message) where T : ViewModelBase
        {
            Instances[typeof(T)](message);
        }
    }

    public interface IViewModelMessage { }
}
